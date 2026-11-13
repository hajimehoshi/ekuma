// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// A synthetic workload shaped like a scene of a 2D tile-based game. Walkers on a grid are
// updated through a prototype chain, a tree of display nodes mirrors them, and plugins
// wrap the hot methods by saving the previous method and calling it through apply.

var TILE = 32;
var COLS = 19;
var ROWS = 11;

// Mover is something with a cell on the grid and a drawn position that follows it.
function Mover() {
    this.init.apply(this, arguments);
}

Mover.prototype.init = function() {
    this.cellX = 0;
    this.cellY = 0;
    this.posX = 0;
    this.posY = 0;
    this.speed = 3;
    this.heading = 0;
    this.step = 0;
    this.stepTimer = 0;
    this.restTimer = 0;
    this.fade = 1;
    this.hidden = false;
    this.skin = 0;
};

Mover.prototype.velocity = function() {
    return this.speed * this.speed / 64;
};

Mover.prototype.walking = function() {
    return this.posX !== this.cellX || this.posY !== this.cellY;
};

Mover.prototype.currentStep = function() {
    return this.step;
};

Mover.prototype.currentHeading = function() {
    return this.heading;
};

Mover.prototype.currentFade = function() {
    return this.fade;
};

Mover.prototype.isHidden = function() {
    return this.hidden;
};

Mover.prototype.pixelX = function() {
    return Math.round(this.posX * TILE) + TILE / 2;
};

Mover.prototype.pixelY = function() {
    return Math.round(this.posY * TILE) + TILE - 4;
};

Mover.prototype.depth = function() {
    return 1 + this.cellY;
};

Mover.prototype.tick = function() {
    if (this.walking()) {
        this.glide();
    } else {
        this.rest();
    }
    this.animate();
};

function approach(from, to, amount) {
    var gap = to - from;
    if (gap > amount) {
        return from + amount;
    }
    if (gap < -amount) {
        return from - amount;
    }
    return to;
}

Mover.prototype.glide = function() {
    var v = this.velocity();
    this.posX = approach(this.posX, this.cellX, v);
    this.posY = approach(this.posY, this.cellY, v);
};

// Headings: 0 east, 1 south, 2 west, 3 north.
var HEADING_DX = [1, 0, -1, 0];
var HEADING_DY = [0, 1, 0, -1];

Mover.prototype.rest = function() {
    this.restTimer += 1;
    if (this.restTimer < 24) {
        return;
    }
    this.restTimer = 0;
    this.heading = (this.heading + 1 + (this.cellX & 1)) & 3;
    this.cellX = (this.cellX + HEADING_DX[this.heading] + COLS) % COLS;
    this.cellY = (this.cellY + HEADING_DY[this.heading] + ROWS) % ROWS;
};

Mover.prototype.animate = function() {
    this.stepTimer += 1;
    if (this.stepTimer * this.speed >= 30) {
        this.stepTimer = 0;
        this.step = this.step === 2 ? 0 : this.step + 1;
    }
};

// Walker is a Mover with an id and an optional trigger.
function Walker() {
    this.init.apply(this, arguments);
}

Walker.prototype = Object.create(Mover.prototype);
Walker.prototype.constructor = Walker;

Walker.prototype.init = function(id) {
    Mover.prototype.init.call(this);
    this.id = id;
    this.mode = id % 4;
    this.armed = false;
    this.cellX = (id * 7) % COLS;
    this.cellY = (id * 3) % ROWS;
    this.posX = this.cellX;
    this.posY = this.cellY;
    this.heading = id & 3;
    this.restTimer = (id * 5) % 24;
};

Walker.prototype.tick = function() {
    Mover.prototype.tick.call(this);
    this.checkTrigger();
};

Walker.prototype.checkTrigger = function() {
    if (this.mode === 2 && this.cellX === this.cellY) {
        this.armed = true;
    }
};

function Field() {
    this.walkers = [];
}

Field.prototype.tick = function() {
    var list = this.walkers;
    for (var i = 0; i < list.length; i++) {
        list[i].tick();
    }
};

function Vec(x, y) {
    this.x = x;
    this.y = y;
}

// Node is an element of the display tree with a 2D affine transform.
function Node() {
    this.init.apply(this, arguments);
}

Node.prototype.init = function() {
    this.parent = null;
    this.kids = [];
    this.x = 0;
    this.y = 0;
    this.angle = 0;
    this.scale = new Vec(1, 1);
    this.opacity = 1;
    this.shown = true;
    this.angleSeen = 0;
    this.sin = 0;
    this.cos = 1;
    this.m00 = 1;
    this.m01 = 0;
    this.m10 = 0;
    this.m11 = 1;
    this.tx = 0;
    this.ty = 0;
    this.netOpacity = 1;
};

Node.prototype.attach = function(kid) {
    this.kids.push(kid);
    kid.parent = this;
    return kid;
};

Node.prototype.tick = function() {
    var kids = this.kids;
    for (var i = 0; i < kids.length; i++) {
        var kid = kids[i];
        if (kid.tick) {
            kid.tick();
        }
    }
};

Node.prototype.layout = function() {
    if (this.angleSeen !== this.angle) {
        this.angleSeen = this.angle;
        this.sin = Math.sin(this.angle);
        this.cos = Math.cos(this.angle);
    }
    // Local matrix rows: [l00 l01] and [l10 l11].
    var l00 = this.cos * this.scale.x;
    var l01 = -this.sin * this.scale.y;
    var l10 = this.sin * this.scale.x;
    var l11 = this.cos * this.scale.y;
    var up = this.parent;
    if (up === null) {
        this.m00 = l00;
        this.m01 = l01;
        this.m10 = l10;
        this.m11 = l11;
        this.tx = this.x;
        this.ty = this.y;
        this.netOpacity = this.opacity;
    } else {
        this.m00 = up.m00 * l00 + up.m01 * l10;
        this.m01 = up.m00 * l01 + up.m01 * l11;
        this.m10 = up.m10 * l00 + up.m11 * l10;
        this.m11 = up.m10 * l01 + up.m11 * l11;
        this.tx = up.m00 * this.x + up.m01 * this.y + up.tx;
        this.ty = up.m10 * this.x + up.m11 * this.y + up.ty;
        this.netOpacity = up.netOpacity * this.opacity;
    }
    var kids = this.kids;
    var n = kids.length;
    for (var i = 0; i < n; i++) {
        if (kids[i].shown) {
            kids[i].layout();
        }
    }
};

// Picture is a Node that shows a rectangle cut out of a texture.
function Picture() {
    this.init.apply(this, arguments);
}

Picture.prototype = Object.create(Node.prototype);
Picture.prototype.constructor = Picture;

Picture.prototype.init = function() {
    Node.prototype.init.call(this);
    this.cut = { left: 0, top: 0, w: 0, h: 0 };
    this.recuts = 0;
    this.tint = 0;
    this.blend = 0;
    this.pivot = new Vec(0, 0);
    this.order = 0;
};

Picture.prototype.tick = function() {
    Node.prototype.tick.call(this);
};

Picture.prototype.cutOut = function(left, top, w, h) {
    var c = this.cut;
    if (c.left === left && c.top === top && c.w === w && c.h === h) {
        return;
    }
    c.left = left;
    c.top = top;
    c.w = w;
    c.h = h;
    this.recuts += 1;
};

// WalkerView is the Picture that draws one Walker.
function WalkerView() {
    this.init.apply(this, arguments);
}

WalkerView.prototype = Object.create(Picture.prototype);
WalkerView.prototype.constructor = WalkerView;

WalkerView.prototype.init = function(walker) {
    Picture.prototype.init.call(this);
    this.walker = walker;
    this.skinShown = -1;
    this.bubble = 0;
    this.extra = 0;
    this.pivot.x = 0.5;
    this.pivot.y = 1;
};

WalkerView.prototype.tick = function() {
    Picture.prototype.tick.call(this);
    this.syncSkin();
    this.syncCut();
    this.syncPlace();
    this.syncLook();
    this.syncShown();
};

WalkerView.prototype.skinChanged = function() {
    return this.skinShown !== this.walker.skin;
};

WalkerView.prototype.syncSkin = function() {
    if (this.skinChanged()) {
        this.skinShown = this.walker.skin;
    }
};

WalkerView.prototype.sheetColumn = function() {
    return 3 * (this.walker.id & 3);
};

WalkerView.prototype.sheetRow = function() {
    return 4 * ((this.walker.id >> 2) & 1);
};

WalkerView.prototype.stepColumn = function() {
    return this.walker.currentStep();
};

WalkerView.prototype.headingRow = function() {
    return this.walker.currentHeading();
};

WalkerView.prototype.syncCut = function() {
    var left = TILE * (this.sheetColumn() + this.stepColumn());
    var top = TILE * (this.sheetRow() + this.headingRow());
    this.cutOut(left, top, TILE, TILE);
};

WalkerView.prototype.syncPlace = function() {
    this.x = this.walker.pixelX();
    this.y = this.walker.pixelY();
    this.order = this.walker.depth();
};

WalkerView.prototype.syncLook = function() {
    this.opacity = this.walker.currentFade();
    this.blend = 0;
};

WalkerView.prototype.syncShown = function() {
    this.shown = this.walker.isHidden() === false;
};

var field = null;
var stage = null;
var views = null;

// installPlugins wraps hot methods count times, each time keeping the previous method and
// calling it through apply or call, so every wrapped call goes through a chain of closures.
function installPlugins(count) {
    for (var p = 0; p < count; p++) {
        installPlugin(p);
    }
}

function installPlugin(p) {
    var viewTick = WalkerView.prototype.tick;
    WalkerView.prototype.tick = function() {
        viewTick.apply(this, arguments);
        this.extra = (this.extra + p + 1) % 997;
    };

    var walkerTick = Walker.prototype.tick;
    Walker.prototype.tick = function() {
        walkerTick.apply(this, arguments);
        if (this.armed) {
            this.restTimer = 0;
            this.armed = false;
        }
    };

    if (p % 3 !== 0) {
        return;
    }

    var syncPlace = WalkerView.prototype.syncPlace;
    WalkerView.prototype.syncPlace = function() {
        syncPlace.call(this);
        this.y += p & 1;
    };

    var pixelX = Mover.prototype.pixelX;
    Mover.prototype.pixelX = function() {
        return pixelX.apply(this, arguments) - (p & 1);
    };
}

function setup(walkerCount, pluginCount) {
    installPlugins(pluginCount);

    field = new Field();
    stage = new Node();
    views = [];
    var world = stage.attach(new Picture());
    var layer = world.attach(new Picture());
    for (var i = 1; i <= walkerCount; i++) {
        var walker = new Walker(i);
        field.walkers.push(walker);
        var view = new WalkerView(walker);
        view.attach(new Picture());
        view.attach(new Picture());
        layer.attach(view);
        views.push(view);
    }
    return views.length;
}

function frame() {
    field.tick();
    stage.tick();
    stage.layout();
}

function digest() {
    var h = 0;
    for (var i = 0; i < views.length; i++) {
        var v = views[i];
        h = (h * 31 + v.tx + 5 * v.ty + v.extra + v.cut.left + 3 * v.cut.top + v.recuts) % 1000000007;
    }
    return h;
}

function run(frames) {
    for (var i = 0; i < frames; i++) {
        frame();
    }
    return digest();
}
